import * as React from 'react';
import { useEffect, useState } from 'react';
import { Redirect, useParams } from 'react-router-dom';
import { Button, Col, FormGroup, Input, Label } from 'reactstrap';
import * as sjcl from 'sjcl';
import Error from './Error';

const displaySecret = (props: any & React.HTMLAttributes<HTMLElement>) => {
  const [loading, setLoading] = useState(false);
  const [error, showError] = useState(false);
  const [secret, setSecret] = useState('');
  const { key, password } = useParams();

  const decrypt = async (pass: string) => {
    setLoading(true);
    const url = process.env.REACT_APP_BACKEND_URL
      ? `${process.env.REACT_APP_BACKEND_URL}/secret`
      : '/secret';
    try {
      const request = await fetch(`${url}/${key}`);
      if (request.status === 200) {
        const data = await request.json();
        setSecret(sjcl.decrypt(pass, data.message));
        setLoading(false);
        return;
      }
    } catch (e) {
      console.log(e);
    }
    setLoading(false);
    showError(true);
  };

  useEffect(() => {
    if (password) {
      decrypt(password);
    }
  }, [password]);

  return (
    <div>
      {loading && (
        <h3>
          Fetching from database and decrypting in browser, please hold...
        </h3>
      )}
      <Error display={error} />
      <Secret secret={secret} />
      <Form display={!password} uuid={key} />
    </div>
  );
};

const Form = (
  props: {
    readonly display: boolean;
    readonly uuid: string | undefined;
  } & React.HTMLAttributes<HTMLElement>,
) => {
  const [password, setPassword] = useState('');
  const [redirect, setRedirect] = useState(false);

  if (redirect) {
    return <Redirect to={`/s/${props.uuid}/${password}`} />;
  }
  return props.display ? (
    <Col sm="6">
      <FormGroup>
        <Label>A decryption key is required, please enter it below</Label>
        <Input
          type="text"
          autoFocus={true}
          placeholder="Decryption Key"
          value={password}
          onChange={e => setPassword(e.target.value)}
        />
      </FormGroup>
      <Button block={true} size="lg" onClick={() => setRedirect(true)}>
        Decrypt Secret
      </Button>
    </Col>
  ) : null;
};

const Secret = (
  props: { readonly secret: string } & React.HTMLAttributes<HTMLElement>,
) =>
  props.secret ? (
    <div>
      <h1>Decrypted Message</h1>
      This secret will not be viewable again, make sure to save it now!
      <pre>{props.secret}</pre>
    </div>
  ) : null;

export default displaySecret;
